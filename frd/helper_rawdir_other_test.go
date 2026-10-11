//go:build !darwin && !linux && !windows

package frd

import (
	"os"
	"sync"
	"testing"
)

// rawFiles keeps each descriptor's os.File reachable until closeRaw, so its
// finalizer cannot close it. openRaw exists here only so the shared tests
// build; AppendHandle is unsupported on these platforms.
var rawFiles sync.Map

func openRaw(tb testing.TB, path string) uintptr {
	tb.Helper()
	f, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	rawFiles.Store(f.Fd(), f)
	return f.Fd()
}

func closeRaw(fd uintptr) error {
	f, _ := rawFiles.LoadAndDelete(fd)
	return f.(*os.File).Close()
}
