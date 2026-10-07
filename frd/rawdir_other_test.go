//go:build !darwin && !linux && !windows

package frd

import (
	"os"
	"testing"
)

// openRaw keeps an os.File alive behind the descriptor, so the tests build
// here; AppendHandle is unsupported on these platforms.
func openRaw(tb testing.TB, path string) (uintptr, func() error) {
	tb.Helper()
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	return f.Fd(), f.Close
}
