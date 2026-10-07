//go:build darwin || linux

package frd

import (
	"testing"

	"golang.org/x/sys/unix"
)

// openRaw opens path as a bare descriptor, without an os.File, the way a
// walker that manages its own descriptors would.
func openRaw(tb testing.TB, path string) (uintptr, func() error) {
	tb.Helper()
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		tb.Fatal(err)
	}
	return uintptr(fd), func() error { return unix.Close(fd) }
}
