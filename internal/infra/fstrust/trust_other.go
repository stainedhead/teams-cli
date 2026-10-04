//go:build !unix

package fstrust

import (
	"errors"
	"os"
)

var errUnsupported = errors.New("ownership check is not supported on this platform")

func realEUID() uint32                        { return ^uint32(0) }
func realLstat(string) (Info, error)          { return Info{}, errUnsupported }
func realFstat(*os.File) (Info, error)        { return Info{}, errUnsupported }
func openNoFollow(p string) (*os.File, error) { return nil, errUnsupported }
