//go:build unix

package fstrust

import (
	"os"
	"syscall"
)

func realEUID() uint32 { return uint32(os.Geteuid()) }

func toInfo(fi os.FileInfo) Info {
	si := Info{Perm: fi.Mode().Perm()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		si.UID = st.Uid
	} else {
		si.UID = ^uint32(0) // unknown owner: never trusted
	}
	switch m := fi.Mode(); {
	case m&os.ModeSymlink != 0:
		si.Kind = KindSymlink
	case m.IsDir():
		si.Kind = KindDir
	case m.IsRegular():
		si.Kind = KindRegular
	}
	return si
}

func realLstat(p string) (Info, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return Info{}, err
	}
	return toInfo(fi), nil
}

func realFstat(f *os.File) (Info, error) {
	fi, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	return toInfo(fi), nil
}

// openNoFollow opens p read-only and refuses to follow a symlink in the last
// component.
func openNoFollow(p string) (*os.File, error) {
	return os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
