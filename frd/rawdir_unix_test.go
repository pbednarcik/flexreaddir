//go:build darwin || linux

package frd

import (
	"testing"

	"golang.org/x/sys/unix"
)

// openRaw opens path as a bare descriptor, without an os.File, the way a
// walker that manages its own descriptors would; closeRaw closes it.
func openRaw(tb testing.TB, path string) uintptr {
	tb.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		tb.Fatal(err)
	}
	return uintptr(fd)
}

func closeRaw(fd uintptr) error { return unix.Close(int(fd)) }
